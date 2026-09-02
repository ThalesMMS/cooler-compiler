class Broken {
  value Int;
  method(x Int) Object { if true 1 else 0 };
};
class Main {
  main() : Object {
    {
      let item Int in item;
      case self of
        branch Object self;
      esac;
      self@Object.missing(1,);
    }
  };
};
