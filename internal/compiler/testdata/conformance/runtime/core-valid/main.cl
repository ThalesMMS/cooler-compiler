class A {
  first : Int <- 1;
  label() : String { "A" };
};
class B inherits A {
  second : Int <- 2;
  label() : String { "B" };
  total() : Int { first + second };
};
class Main inherits IO {
  main() : Object {
    let item : A <- new B in {
      out_string(item.label());
      case item of
        child : B => out_int(child.total());
        parent : A => out_int(1);
      esac;
    }
  };
};
