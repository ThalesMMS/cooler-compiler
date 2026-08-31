class Animal {
  name() : String { "animal" };
};

class Cat inherits Animal {
  name() : String { "cat" };
};

class Main inherits IO {
  main() : Object {
    let pet : Animal <- new Cat in
      case pet of
        cat : Cat => out_string(cat.name().concat("\n"));
        animal : Animal => out_string(animal.name().concat("\n"));
        other : Object => out_string("object\n");
      esac
  };
};
